package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/HouzuoGuo/tiedot/db"
	"github.com/PuerkitoBio/goquery"
	"github.com/bwmarrin/discordgo"
	"github.com/dghubble/go-twitter/twitter"
	"github.com/dghubble/oauth1"
	"github.com/hashicorp/go-version"
	"github.com/mvdan/xurls"
	"golang.org/x/net/html"
	"gopkg.in/ini.v1"
)

var (
	ChannelWhitelist             map[string]string
	InteractiveChannelWhitelist  map[string]string
	BaseDownloadPath             string
	RegexpUrlTwitter             *regexp.Regexp
	RegexpUrlTwitterStatus       *regexp.Regexp
	RegexpUrlTistory             *regexp.Regexp
	RegexpUrlTistoryWithCDN      *regexp.Regexp
	RegexpUrlGfycat              *regexp.Regexp
	RegexpUrlInstagram           *regexp.Regexp
	RegexpUrlImgurSingle         *regexp.Regexp
	RegexpUrlImgurAlbum          *regexp.Regexp
	RegexpUrlGoogleDrive         *regexp.Regexp
	RegexpUrlPossibleTistorySite *regexp.Regexp
	RegexpUrlFlickrPhoto         *regexp.Regexp
	RegexpUrlFlickrAlbum         *regexp.Regexp
	dg                           *discordgo.Session
	DownloadTistorySites         bool
	interactiveChannelLinkTemp   map[string]string
	DiscordUserId                string
	myDB                         *db.DB
	historyCommandActive         map[string]string
	MaxDownloadRetries           int
	flickrApiKey                 string
	twitterConsumerKey           string
	twitterConsumerSecret        string
	twitterAccessToken           string
	twitterAccessTokenSecret     string
)

const (
	VERSION                          string = "1.15"
	DATABASE_DIR                     string = "database"
	RELEASE_URL                      string = "https://github.com/Seklfreak/discord-image-downloader-go/releases/latest"
	RELEASE_API_URL                  string = "https://api.github.com/repos/Seklfreak/discord-image-downloader-go/releases/latest"
	IMGUR_CLIENT_ID                  string = "a39473314df3f59"
	REGEXP_URL_TWITTER               string = `^http(s?):\/\/pbs\.twimg\.com\/media\/[^\./]+\.(jpg|png)((\:[a-z]+)?)$`
	REGEXP_URL_TWITTER_STATUS        string = `^http(s?):\/\/(www\.)?twitter\.com\/([A-Za-z0-9-_\.]+\/status\/|statuses\/)([0-9]+)$`
	REGEXP_URL_TISTORY               string = `^http(s?):\/\/[a-z0-9]+\.uf\.tistory\.com\/(image|original)\/[A-Z0-9]+$`
	REGEXP_URL_TISTORY_WITH_CDN      string = `^http(s)?:\/\/[0-9a-z]+.daumcdn.net\/[a-z]+\/[a-zA-Z0-9\.]+\/\?scode=mtistory&fname=http(s?)%3A%2F%2F[a-z0-9]+\.uf\.tistory\.com%2F(image|original)%2F[A-Z0-9]+$`
	REGEXP_URL_GFYCAT                string = `^http(s?):\/\/gfycat\.com\/[A-Za-z]+$`
	REGEXP_URL_INSTAGRAM             string = `^http(s?):\/\/(www\.)?instagram\.com\/p\/[^/]+\/(\?[^/]+)?$`
	REGEXP_URL_IMGUR_SINGLE          string = `^http(s?):\/\/(i\.)?imgur\.com\/[A-Za-z0-9]+(\.gifv)?$`
	REGEXP_URL_IMGUR_ALBUM           string = `^http(s?):\/\/imgur\.com\/a\/[A-Za-z0-9]+$`
	REGEXP_URL_GOOGLEDRIVE           string = `^http(s?):\/\/drive\.google\.com\/file\/d\/[^/]+\/view$`
	REGEXP_URL_POSSIBLE_TISTORY_SITE string = `^http(s)?:\/\/[0-9a-zA-Z\.-]+\/(m\/)?(photo\/)?[0-9]+$`
	REGEXP_URL_FLICKR_PHOTO          string = `^http(s)?:\/\/(www\.)?flickr\.com\/photos\/([0-9]+)@([A-Z0-9]+)\/([0-9]+)(\/)?(\/in\/album-([0-9]+)(\/)?)?$`
	REGEXP_URL_FLICKR_ALBUM          string = `^http(s)?:\/\/(www\.)?flickr\.com\/photos\/([0-9]+)@([A-Z0-9]+)\/albums\/(with\/)?([0-9]+)(\/)?$`
)

type GfycatObject struct {
	GfyItem map[string]string
}

type ImgurAlbumObject struct {
	Data []struct {
		Link string
	}
}

func main() {
	fmt.Printf("discord-image-downloader-go version %s\n", VERSION)
	if !isLatestRelease() {
		fmt.Printf("update available on %s !\n", RELEASE_URL)
	}

	var err error
	cfg, err := ini.Load("config.ini")
	if err != nil {
		fmt.Println("unable to read config file", err)
		cfg = ini.Empty()
	}

	if (!cfg.Section("auth").HasKey("email") ||
		!cfg.Section("auth").HasKey("password")) &&
		!cfg.Section("auth").HasKey("token") {
		cfg.Section("auth").NewKey("email", "your@email.com")
		cfg.Section("auth").NewKey("password", "your password")
		cfg.Section("general").NewKey("skip edits", "true")
		cfg.Section("general").NewKey("download tistory sites", "false")
		cfg.Section("general").NewKey("max download retries", "3")
		cfg.Section("channels").NewKey("channelid1", "C:\\full\\path\\1")
		cfg.Section("channels").NewKey("channelid2", "C:\\full\\path\\2")
		cfg.Section("channels").NewKey("channelid3", "C:\\full\\path\\3")
		cfg.Section("flickr").NewKey("api key", "your flickr api key")
		cfg.Section("twitter").NewKey("consumer key", "your consumer key")
		cfg.Section("twitter").NewKey("consumer secret", "your consumer secret")
		cfg.Section("twitter").NewKey("access token", "your access token")
		cfg.Section("twitter").NewKey("access token secret", "your access token secret")
		err = cfg.SaveTo("config.ini")

		if err != nil {
			fmt.Println("unable to write config file", err)
			return
		}
		fmt.Println("Wrote config file, please fill out and restart the program")
		return
	}

	myDB, err = db.OpenDB(DATABASE_DIR)
	if err != nil {
		fmt.Println("unable to create db", err)
		return
	}
	if myDB.Use("Downloads") == nil {
		if err := myDB.Create("Downloads"); err != nil {
			fmt.Println("unable to create db", err)
			return
		}
		if err := myDB.Use("Downloads").Index([]string{"Url"}); err != nil {
			fmt.Println("unable to create index", err)
			return
		}
	}

	ChannelWhitelist = cfg.Section("channels").KeysHash()
	InteractiveChannelWhitelist = cfg.Section("interactive channels").KeysHash()
	interactiveChannelLinkTemp = make(map[string]string)
	historyCommandActive = make(map[string]string)
	flickrApiKey = cfg.Section("flickr").Key("api key").MustString("yourflickrapikey")
	twitterConsumerKey = cfg.Section("twitter").Key("consumer key").MustString("your consumer key")
	twitterConsumerSecret = cfg.Section("twitter").Key("consumer secret").MustString("your consumer secret")
	twitterAccessToken = cfg.Section("twitter").Key("access token").MustString("your access token")
	twitterAccessTokenSecret = cfg.Section("twitter").Key("access token secret").MustString("your access token secret")

	RegexpUrlTwitter, err = regexp.Compile(REGEXP_URL_TWITTER)
	if err != nil {
		fmt.Println("Regexp error", err)
		return
	}
	RegexpUrlTwitterStatus, err = regexp.Compile(REGEXP_URL_TWITTER_STATUS)
	if err != nil {
		fmt.Println("Regexp error", err)
		return
	}
	RegexpUrlTistory, err = regexp.Compile(REGEXP_URL_TISTORY)
	if err != nil {
		fmt.Println("Regexp error", err)
		return
	}
	RegexpUrlTistoryWithCDN, err = regexp.Compile(REGEXP_URL_TISTORY_WITH_CDN)
	if err != nil {
		fmt.Println("Regexp error", err)
		return
	}
	RegexpUrlGfycat, err = regexp.Compile(REGEXP_URL_GFYCAT)
	if err != nil {
		fmt.Println("Regexp error", err)
		return
	}
	RegexpUrlInstagram, err = regexp.Compile(REGEXP_URL_INSTAGRAM)
	if err != nil {
		fmt.Println("Regexp error", err)
		return
	}
	RegexpUrlImgurSingle, err = regexp.Compile(REGEXP_URL_IMGUR_SINGLE)
	if err != nil {
		fmt.Println("Regexp error", err)
		return
	}
	RegexpUrlImgurAlbum, err = regexp.Compile(REGEXP_URL_IMGUR_ALBUM)
	if err != nil {
		fmt.Println("Regexp error", err)
		return
	}
	RegexpUrlGoogleDrive, err = regexp.Compile(REGEXP_URL_GOOGLEDRIVE)
	if err != nil {
		fmt.Println("Regexp error", err)
		return
	}
	RegexpUrlPossibleTistorySite, err = regexp.Compile(REGEXP_URL_POSSIBLE_TISTORY_SITE)
	if err != nil {
		fmt.Println("Regexp error", err)
		return
	}
	RegexpUrlFlickrPhoto, err = regexp.Compile(REGEXP_URL_FLICKR_PHOTO)
	if err != nil {
		fmt.Println("Regexp error", err)
		return
	}
	RegexpUrlFlickrAlbum, err = regexp.Compile(REGEXP_URL_FLICKR_ALBUM)
	if err != nil {
		fmt.Println("Regexp error", err)
		return
	}

	if cfg.Section("auth").HasKey("token") {
		dg, err = discordgo.New(cfg.Section("auth").Key("token").String())
	} else {
		dg, err = discordgo.New(
			cfg.Section("auth").Key("email").String(),
			cfg.Section("auth").Key("password").String())
	}
	if err != nil {
		fmt.Println("error creating Discord session,", err)
		return
	}

	dg.AddHandler(messageCreate)

	if cfg.Section("general").HasKey("skip edits") {
		if cfg.Section("general").Key("skip edits").MustBool() == false {
			dg.AddHandler(messageUpdate)
		}
	}

	DownloadTistorySites = cfg.Section("general").Key("download tistory sites").MustBool()
	MaxDownloadRetries = cfg.Section("general").Key("max download retries").MustInt(3)

	err = dg.Open()
	if err != nil {
		fmt.Println("error opening connection,", err)
		return
	}

	u, err := dg.User("@me")
	if err != nil {
		fmt.Println("error obtaining account details,", err)
	}

	fmt.Printf("Client is now connected as %s. Press CTRL-C to exit.\n",
		u.Username)
	DiscordUserId = u.ID

	updateDiscordStatus()

	// keep program running until CTRL-C is pressed.
	<-make(chan struct{})
	myDB.Close()
	return
}

func messageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	handleDiscordMessage(m.Message)
}

func messageUpdate(s *discordgo.Session, m *discordgo.MessageUpdate) {
	handleDiscordMessage(m.Message)
}

func getDownloadLinks(url string) map[string]string {
	if RegexpUrlTwitter.MatchString(url) {
		links, err := getTwitterUrls(url)
		if err != nil {
			fmt.Println("twitter url failed,", url, ",", err)
		} else if len(links) > 0 {
			return links
		}
	}
	if RegexpUrlTwitterStatus.MatchString(url) {
		links, err := getTwitterStatusUrls(url)
		if err != nil {
			fmt.Println("twitter status url failed,", url, ",", err)
		} else if len(links) > 0 {
			return links
		}
	}
	if RegexpUrlTistory.MatchString(url) {
		links, err := getTistoryUrls(url)
		if err != nil {
			fmt.Println("tistory url failed,", url, ",", err)
		} else if len(links) > 0 {
			return links
		}
	}
	if RegexpUrlGfycat.MatchString(url) {
		links, err := getGfycatUrls(url)
		if err != nil {
			fmt.Println("gfycat url failed,", url, ",", err)
		} else if len(links) > 0 {
			return links
		}
	}
	if RegexpUrlInstagram.MatchString(url) {
		links, err := getInstagramUrls(url)
		if err != nil {
			fmt.Println("instagram url failed,", url, ",", err)
		} else if len(links) > 0 {
			return links
		}
	}
	if RegexpUrlImgurSingle.MatchString(url) {
		links, err := getImgurSingleUrls(url)
		if err != nil {
			fmt.Println("imgur single url failed, ", url, ",", err)
		} else if len(links) > 0 {
			return links
		}
	}
	if RegexpUrlImgurAlbum.MatchString(url) {
		links, err := getImgurAlbumUrls(url)
		if err != nil {
			fmt.Println("imgur album url failed, ", url, ",", err)
		} else if len(links) > 0 {
			return links
		}
	}
	if RegexpUrlGoogleDrive.MatchString(url) {
		links, err := getGoogleDriveUrls(url)
		if err != nil {
			fmt.Println("google drive album url failed, ", url, ",", err)
		} else if len(links) > 0 {
			return links
		}
	}
	if RegexpUrlFlickrPhoto.MatchString(url) {
		links, err := getFlickrPhotoUrls(url)
		if err != nil {
			fmt.Println("flickr photo url failed, ", url, ",", err)
		} else if len(links) > 0 {
			return links
		}
	}
	if RegexpUrlFlickrAlbum.MatchString(url) {
		links, err := getFlickrAlbumUrls(url)
		if err != nil {
			fmt.Println("flickr album url failed, ", url, ",", err)
		} else if len(links) > 0 {
			return links
		}
	}
	if DownloadTistorySites {
		if RegexpUrlPossibleTistorySite.MatchString(url) {
			links, err := getPossibleTistorySiteUrls(url)
			if err != nil {
				fmt.Println("checking for tistory site failed, ", url, ",", err)
			} else if len(links) > 0 {
				return links
			}
		}
	}
	return map[string]string{url: ""}
}

func handleDiscordMessage(m *discordgo.Message) {
	if folderName, ok := ChannelWhitelist[m.ChannelID]; ok {
		fileTime := time.Now()
		var err error
		if m.Timestamp != "" {
			fileTime, err = m.Timestamp.Parse()
			if err != nil {
				fmt.Println(err)
			}
		}
		for _, iAttachment := range m.Attachments {
			startDownload(iAttachment.URL, iAttachment.Filename, folderName, m.ChannelID, m.Author.ID, fileTime)
		}
		foundUrls := xurls.Strict.FindAllString(m.Content, -1)
		for _, iFoundUrl := range foundUrls {
			links := getDownloadLinks(iFoundUrl)
			for link, filename := range links {
				startDownload(link, filename, folderName, m.ChannelID, m.Author.ID, fileTime)
			}
		}
	} else if folderName, ok := InteractiveChannelWhitelist[m.ChannelID]; ok {
		if DiscordUserId != "" && m.Author != nil && m.Author.ID != DiscordUserId {
			dg.ChannelTyping(m.ChannelID)
			message := strings.ToLower(m.Content)
			_, historyCommandIsActive := historyCommandActive[m.ChannelID]
			switch {
			case message == "help":
				dg.ChannelMessageSend(m.ChannelID,
					"**<link>** to download a link\n**version** to find out the version\n**stats** to view stats\n**channels** to list active channels\n**help** to open this help\n ")
			case message == "version":
				dg.ChannelMessageSend(m.ChannelID, fmt.Sprintf("discord-image-downloder-go **v%s**", VERSION))
				dg.ChannelTyping(m.ChannelID)
				if isLatestRelease() {
					dg.ChannelMessageSend(m.ChannelID, "version is up to date")
				} else {
					dg.ChannelMessageSend(m.ChannelID, fmt.Sprintf("**update available on <%s>**", RELEASE_URL))
				}
			case message == "channels":
				replyMessage := "**channels**\n"
				for channelId, channelFolder := range ChannelWhitelist {
					channel, err := dg.Channel(channelId)
					if err == nil {
						if channel.IsPrivate {
							replyMessage += fmt.Sprintf("@%s (`#%s`): `%s`\n", channel.Recipient.Username, channelId, channelFolder)
						} else {
							guild, err := dg.Guild(channel.GuildID)
							if err == nil {
								replyMessage += fmt.Sprintf("#%s/%s (`#%s`): `%s`\n", guild.Name, channel.Name, channelId, channelFolder)
							}
						}
					}
				}
				replyMessage += "**interactive channels**\n"
				for channelId, channelFolder := range InteractiveChannelWhitelist {
					channel, err := dg.Channel(channelId)
					if err == nil {
						if channel.IsPrivate {
							replyMessage += fmt.Sprintf("@%s (`#%s`): `%s`\n", channel.Recipient.Username, channelId, channelFolder)
						} else {
							guild, err := dg.Guild(channel.GuildID)
							if err == nil {
								replyMessage += fmt.Sprintf("#%s/%s (`#%s`): `%s`\n", guild.Name, channel.Name, channelId, channelFolder)
							}
						}
					}
				}
				dg.ChannelMessageSend(m.ChannelID, replyMessage)
			case message == "stats":
				dg.ChannelTyping(m.ChannelID)
				channelStats := make(map[string]int)
				userStats := make(map[string]int)
				userGuilds := make(map[string]string)
				i := 0
				myDB.Use("Downloads").ForEachDoc(func(id int, docContent []byte) (willMoveOn bool) {
					downloadedImage := findDownloadedImageById(id)
					channelStats[downloadedImage.ChannelId] += 1
					userStats[downloadedImage.UserId] += 1
					if _, ok := userGuilds[downloadedImage.UserId]; !ok {
						channel, err := dg.Channel(downloadedImage.ChannelId)
						if err == nil && channel.GuildID != "" {
							userGuilds[downloadedImage.UserId] = channel.GuildID
						}
					}
					i++
					return true
				})
				channelStatsSorted := sortStringIntMapByValue(channelStats)
				userStatsSorted := sortStringIntMapByValue(userStats)
				replyMessage := fmt.Sprintf("I downloaded **%d** pictures in **%d** channels by **%d** users\n", i, len(channelStats), len(userStats))
				replyMessage += "**channel breakdown**\n"
				for _, downloads := range channelStatsSorted {
					channel, err := dg.Channel(downloads.Key)
					if err == nil {
						if channel.IsPrivate {
							replyMessage += fmt.Sprintf("@%s (`#%s`): **%d** downloads\n", channel.Recipient.Username, downloads.Key, downloads.Value)
						} else {
							guild, err := dg.Guild(channel.GuildID)
							if err == nil {
								replyMessage += fmt.Sprintf("#%s/%s (`#%s`): **%d** downloads\n", guild.Name, channel.Name, downloads.Key, downloads.Value)
							} else {
								fmt.Println(err)
							}
						}
					} else {
						fmt.Println(err)
					}
				}
				replyMessage += "**user breakdown**\n"
				userI := 0
				for _, downloads := range userStatsSorted {
					userI++
					if userI > 10 {
						replyMessage += "_only the top 10 users get shown_\n"
						break
					}
					if guildId, ok := userGuilds[downloads.Key]; ok {
						user, err := dg.GuildMember(guildId, downloads.Key)
						if err == nil {
							replyMessage += fmt.Sprintf("@%s: **%d** downloads\n", user.User.Username, downloads.Value)
						} else {
							replyMessage += fmt.Sprintf("@`%s`: **%d** downloads\n", downloads.Key, downloads.Value)
						}
					} else {
						replyMessage += fmt.Sprintf("@`%s`: **%d** downloads\n", downloads.Key, downloads.Value)
					}
				}
				dg.ChannelMessageSend(m.ChannelID, replyMessage)
			case message == "history", historyCommandIsActive:
				i := 0
				_, historyCommandIsSet := historyCommandActive[m.ChannelID]
				if !historyCommandIsSet || historyCommandActive[m.ChannelID] == "" {
					historyCommandActive[m.ChannelID] = ""
					if folder, ok := ChannelWhitelist[m.Content]; ok {
						dg.ChannelMessageSend(m.ChannelID, fmt.Sprintf("downloading to `%s`", folder))
						historyCommandActive[m.ChannelID] = "downloading"
						lastBefore := ""
						var lastBeforeTime time.Time
					MessageRequestingLoop:
						for true {
							if lastBeforeTime != (time.Time{}) {
								fmt.Printf("[%s] Requesting 100 more messages, (before %s)\n", time.Now().Format(time.Stamp), lastBeforeTime)
								dg.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Requesting 100 more messages, (before %s)\n", lastBeforeTime))
							}
							messages, err := dg.ChannelMessages(m.Content, 100, lastBefore, "")
							if err == nil {
								if len(messages) <= 0 {
									delete(historyCommandActive, m.ChannelID)
									break MessageRequestingLoop
								}
								lastBefore = messages[len(messages)-1].ID
								lastBeforeTime, err = messages[len(messages)-1].Timestamp.Parse()
								if err != nil {
									fmt.Println(err)
								}
								for _, message := range messages {
									fileTime := time.Now()
									if m.Timestamp != "" {
										fileTime, err = message.Timestamp.Parse()
										if err != nil {
											fmt.Println(err)
										}
									}
									if historyCommandActive[m.ChannelID] == "cancel" {
										delete(historyCommandActive, m.ChannelID)
										break MessageRequestingLoop
									}
									for _, iAttachment := range message.Attachments {
										if findDownloadedImageByUrl(iAttachment.URL) == nil {
											i++
											startDownload(iAttachment.URL, iAttachment.Filename, folder, message.ChannelID, message.Author.ID, fileTime)
										}
									}
									foundUrls := xurls.Strict.FindAllString(message.Content, -1)
									for _, iFoundUrl := range foundUrls {
										links := getDownloadLinks(iFoundUrl)
										for link, filename := range links {
											if findDownloadedImageByUrl(link) == nil {
												i++
												startDownload(link, filename, folder, message.ChannelID, message.Author.ID, fileTime)
											}
										}
									}
								}
							} else {
								dg.ChannelMessageSend(m.ChannelID, err.Error())
								fmt.Println(err)
								delete(historyCommandActive, m.ChannelID)
								break MessageRequestingLoop
							}
						}
						dg.ChannelMessageSend(m.ChannelID, fmt.Sprintf("done, %d download links started!", i))
					} else {
						dg.ChannelMessageSend(m.ChannelID, "please send me a channel id (from the whitelist)")
					}
				} else if historyCommandActive[m.ChannelID] == "downloading" && message == "cancel" {
					historyCommandActive[m.ChannelID] = "cancel"
				}
			default:
				if link, ok := interactiveChannelLinkTemp[m.ChannelID]; ok {
					fileTime := time.Now()
					var err error
					if m.Timestamp != "" {
						fileTime, err = m.Timestamp.Parse()
						if err != nil {
							fmt.Println(err)
						}
					}
					if m.Content == "." {
						dg.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Download of <%s> started", link))
						dg.ChannelTyping(m.ChannelID)
						delete(interactiveChannelLinkTemp, m.ChannelID)
						links := getDownloadLinks(link)
						for linkR, filename := range links {
							startDownload(linkR, filename, folderName, m.ChannelID, m.Author.ID, fileTime)
						}
						dg.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Download of <%s> finished", link))
					} else if strings.ToLower(m.Content) == "cancel" {
						delete(interactiveChannelLinkTemp, m.ChannelID)
						dg.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Download of <%s> cancelled", link))
					} else if IsValid(m.Content) {
						dg.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Download of <%s> started", link))
						dg.ChannelTyping(m.ChannelID)
						delete(interactiveChannelLinkTemp, m.ChannelID)
						links := getDownloadLinks(link)
						for linkR, filename := range links {
							startDownload(linkR, filename, m.Content, m.ChannelID, m.Author.ID, fileTime)
						}
						dg.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Download of <%s> finished", link))
					} else {
						dg.ChannelMessageSend(m.ChannelID, "invalid path")
					}
				} else {
					_ = folderName
					foundLinks := false
					for _, iAttachment := range m.Attachments {
						dg.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Where do you want to save <%s>?\nType **.** for default path or **cancel** to cancel the download %s", iAttachment.URL, folderName))
						interactiveChannelLinkTemp[m.ChannelID] = iAttachment.URL
						foundLinks = true
					}
					foundUrls := xurls.Strict.FindAllString(m.Content, -1)
					for _, iFoundUrl := range foundUrls {
						dg.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Where do you want to save <%s>?\nType **.** for default path or **cancel** to cancel the download %s", iFoundUrl, folderName))
						interactiveChannelLinkTemp[m.ChannelID] = iFoundUrl
						foundLinks = true
					}
					if foundLinks == false {
						dg.ChannelMessageSend(m.ChannelID, "unable to find valid link")
					}
				}
			}
		}
	}
}

type GithubReleaseApiObject struct {
	TagName string `json:"tag_name"`
}

func isLatestRelease() bool {
	githubReleaseApiObject := new(GithubReleaseApiObject)
	getJson(RELEASE_API_URL, githubReleaseApiObject)
	currentVer, err := version.NewVersion(VERSION)
	if err != nil {
		fmt.Println(err)
		return true
	}
	lastVer, err := version.NewVersion(githubReleaseApiObject.TagName)
	if err != nil {
		fmt.Println(err)
		return true
	}
	if lastVer.GreaterThan(currentVer) {
		return false
	}
	return true
}

// http://stackoverflow.com/a/35240286/1443726
func IsValid(fp string) bool {
	// Check if file already exists
	if _, err := os.Stat(fp); err == nil {
		return true
	}

	// Attempt to create it
	var d []byte
	if err := ioutil.WriteFile(fp, d, 0644); err == nil {
		os.Remove(fp) // And delete it
		return true
	}

	return false
}

func getTwitterUrls(url string) (map[string]string, error) {
	parts := strings.Split(url, ":")
	if len(parts) < 2 {
		return nil, errors.New("unable to parse twitter url")
	} else {
		return map[string]string{"https:" + parts[1] + ":orig": filenameFromUrl(parts[1])}, nil
	}
}

func getTwitterStatusUrls(url string) (map[string]string, error) {
	if (twitterConsumerKey == "" || twitterConsumerKey == "your consumer key") ||
		(twitterConsumerSecret == "" || twitterConsumerSecret == "your consumer secret") ||
		(twitterAccessToken == "" || twitterAccessToken == "your access token") ||
		(twitterAccessTokenSecret == "" || twitterAccessTokenSecret == "your access token secret") {
		return nil, errors.New("invalid twitter api keys set")
	}
	twitterConfig := oauth1.NewConfig(twitterConsumerKey, twitterConsumerSecret)
	twitterToken := oauth1.NewToken(twitterAccessToken, twitterAccessTokenSecret)
	twitterHttpClient := twitterConfig.Client(oauth1.NoContext, twitterToken)
	twitterClient := twitter.NewClient(twitterHttpClient)

	matches := RegexpUrlTwitterStatus.FindStringSubmatch(url)
	statusId, err := strconv.ParseInt(matches[4], 10, 64)
	if err != nil {
		return nil, err
	}

	tweet, _, err := twitterClient.Statuses.Show(statusId, nil)
	if err != nil {
		return nil, err
	}

	links := make(map[string]string)
	for _, tweetMedia := range tweet.Entities.Media {
		if len(tweetMedia.VideoInfo.Variants) > 0 {
			// Not yet in the API (?)
			fmt.Println("please post a link to this tweet here: https://github.com/Seklfreak/discord-image-downloader-go/issues")
			for _, videoVariant := range tweetMedia.VideoInfo.Variants {
				fmt.Printf("%+v\n", videoVariant)
			}
		} // else {
		links[tweetMedia.MediaURLHttps] = ""
		//}
	}
	for _, tweetUrl := range tweet.Entities.Urls {
		foundUrls := getDownloadLinks(tweetUrl.ExpandedURL)
		for foundUrlKey, foundUrlValue := range foundUrls {
			links[foundUrlKey] = foundUrlValue
		}
	}

	return links, nil
}

func getTistoryUrls(url string) (map[string]string, error) {
	url = strings.Replace(url, "/image/", "/original/", -1)
	return map[string]string{url: ""}, nil
}

func getTistoryWithCDNUrls(urlI string) (map[string]string, error) {
	parameters, _ := url.ParseQuery(urlI)
	if val, ok := parameters["fname"]; ok {
		if len(val) > 0 {
			if RegexpUrlTistory.MatchString(val[0]) {
				return getTistoryUrls(val[0])
			}
		}
	}
	return nil, nil
}

func getGfycatUrls(url string) (map[string]string, error) {
	parts := strings.Split(url, "/")
	if len(parts) < 3 {
		return nil, errors.New("unable to parse gfycat url")
	} else {
		gfycatId := parts[len(parts)-1]
		gfycatObject := new(GfycatObject)
		getJson("https://gfycat.com/cajax/get/"+gfycatId, gfycatObject)
		gfycatUrl := gfycatObject.GfyItem["gifUrl"]
		if gfycatUrl == "" {
			gfycatUrl = gfycatObject.GfyItem["mp4Url"]
			fmt.Println("fallback to gfycat mp4")
		}
		if url == "" {
			return nil, errors.New("failed to read response from gfycat")
		} else {
			return map[string]string{gfycatUrl: ""}, nil
		}
	}
}

func getInstagramUrls(url string) (map[string]string, error) {
	// if instagram video
	videoUrl := getInstagramVideoUrl(url)
	if videoUrl != "" {
		return map[string]string{videoUrl: ""}, nil
	}

	// if instagram picture
	afterLastSlash := strings.LastIndex(url, "/")
	mediaUrl := url[:afterLastSlash] + strings.Replace(url[afterLastSlash:], "/", "/media/?size=l", -1)
	mediaUrl = strings.Replace(mediaUrl, "?taken-by=", "&taken-by", -1)
	return map[string]string{mediaUrl: ""}, nil
}

func getImgurSingleUrls(url string) (map[string]string, error) {
	url = strings.Replace(url, "imgur.com/", "imgur.com/download/", -1)
	url = strings.Replace(url, ".gifv", "", -1)
	return map[string]string{url: ""}, nil
}

func getImgurAlbumUrls(url string) (map[string]string, error) {
	afterLastSlash := strings.LastIndex(url, "/")
	albumId := url[afterLastSlash+1:]
	headers := make(map[string]string)
	headers["Authorization"] = "Client-ID " + IMGUR_CLIENT_ID
	imgurAlbumObject := new(ImgurAlbumObject)
	getJsonWithHeaders("https://api.imgur.com/3/album/"+albumId+"/images", imgurAlbumObject, headers)
	links := make(map[string]string)
	for _, v := range imgurAlbumObject.Data {
		links[v.Link] = ""
	}
	fmt.Printf("[%s] Found imgur album with %d images (url: %s)\n", time.Now().Format(time.Stamp), len(links), url)
	return links, nil
}

func getGoogleDriveUrls(url string) (map[string]string, error) {
	parts := strings.Split(url, "/")
	if len(parts) != 7 {
		return nil, errors.New("unable to parse google drive url")
	} else {
		fileId := parts[len(parts)-2]
		return map[string]string{"https://drive.google.com/uc?export=download&id=" + fileId: ""}, nil
	}
}

type FlickrPhotoSizeObject struct {
	Label  string `json:"label"`
	Width  int    `json:"width,int,string"`
	Height int    `json:"height,int,string"`
	Source string `json:"source"`
	URL    string `json:"url"`
	Media  string `json:"media"`
}

type FlickrPhotoObject struct {
	Sizes struct {
		Canblog     int                     `json:"canblog"`
		Canprint    int                     `json:"canprint"`
		Candownload int                     `json:"candownload"`
		Size        []FlickrPhotoSizeObject `json:"size"`
	} `json:"sizes"`
	Stat string `json:"stat"`
}

func getFlickrUrlFromPhotoId(photoId string) string {
	reqUrl := fmt.Sprintf("https://www.flickr.com/services/rest/?format=json&nojsoncallback=1&method=%s&api_key=%s&photo_id=%s",
		"flickr.photos.getSizes", flickrApiKey, photoId)
	flickrPhoto := new(FlickrPhotoObject)
	getJson(reqUrl, flickrPhoto)
	var bestSize FlickrPhotoSizeObject
	for _, size := range flickrPhoto.Sizes.Size {
		if bestSize.Label == "" {
			bestSize = size
		} else {
			if size.Width > bestSize.Width || size.Height > bestSize.Height {
				bestSize = size
			}
		}
	}
	return bestSize.Source
}

func getFlickrPhotoUrls(url string) (map[string]string, error) {
	if flickrApiKey == "" || flickrApiKey == "yourflickrapikey" || flickrApiKey == "your flickr api key" {
		return nil, errors.New("invalid flickr api key set")
	}
	matches := RegexpUrlFlickrPhoto.FindStringSubmatch(url)
	photoId := matches[5]
	if photoId == "" {
		return nil, errors.New("unable to get photo id from url")
	}
	return map[string]string{getFlickrUrlFromPhotoId(photoId): ""}, nil
}

type FlickrAlbumObject struct {
	Photoset struct {
		ID        string `json:"id"`
		Primary   string `json:"primary"`
		Owner     string `json:"owner"`
		Ownername string `json:"ownername"`
		Photo     []struct {
			ID        string `json:"id"`
			Secret    string `json:"secret"`
			Server    string `json:"server"`
			Farm      int    `json:"farm"`
			Title     string `json:"title"`
			Isprimary string `json:"isprimary"`
			Ispublic  int    `json:"ispublic"`
			Isfriend  int    `json:"isfriend"`
			Isfamily  int    `json:"isfamily"`
		} `json:"photo"`
		Page    int    `json:"page"`
		PerPage int    `json:"per_page"`
		Perpage int    `json:"perpage"`
		Pages   int    `json:"pages"`
		Total   string `json:"total"`
		Title   string `json:"title"`
	} `json:"photoset"`
	Stat string `json:"stat"`
}

func getFlickrAlbumUrls(url string) (map[string]string, error) {
	if flickrApiKey == "" || flickrApiKey == "yourflickrapikey" {
		return nil, errors.New("invalid flickr api key set")
	}
	matches := RegexpUrlFlickrAlbum.FindStringSubmatch(url)
	albumId := matches[6]
	if albumId == "" {
		return nil, errors.New("unable to get album id from url")
	}
	reqUrl := fmt.Sprintf("https://www.flickr.com/services/rest/?format=json&nojsoncallback=1&method=%s&api_key=%s&photoset_id=%s&per_page=500",
		"flickr.photosets.getPhotos", flickrApiKey, albumId)
	flickrAlbum := new(FlickrAlbumObject)
	getJson(reqUrl, flickrAlbum)
	links := make(map[string]string)
	for _, photo := range flickrAlbum.Photoset.Photo {
		links[getFlickrUrlFromPhotoId(photo.ID)] = ""
	}
	fmt.Printf("[%s] Found flickr album with %d images (url: %s)\n", time.Now().Format(time.Stamp), len(links), url)
	return links, nil
}

func getPossibleTistorySiteUrls(url string) (map[string]string, error) {
	client := new(http.Client)
	request, err := http.NewRequest("HEAD", url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Add("Accept-Encoding", "identity")
	respHead, err := client.Do(request)
	if err != nil {
		return nil, err
	}

	contentType := ""
	for headerKey, headerValue := range respHead.Header {
		if headerKey == "Content-Type" {
			contentType = headerValue[0]
		}
	}
	if !strings.Contains(contentType, "text/html") {
		return nil, nil
	}

	request, err = http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Add("Accept-Encoding", "identity")
	resp, err := client.Do(request)
	if err != nil {
		return nil, err
	}

	doc, err := goquery.NewDocumentFromResponse(resp)
	if err != nil {
		return nil, err
	}

	var links = make(map[string]string)

	doc.Find(".article img, #content img, div[role=main] img, .section_blogview img").Each(func(i int, s *goquery.Selection) {
		foundUrl, exists := s.Attr("src")
		if exists == true {
			isTistoryCdnUrl := RegexpUrlTistoryWithCDN.MatchString(foundUrl)
			isTistoryUrl := RegexpUrlTistory.MatchString(foundUrl)
			if isTistoryCdnUrl == true {
				finalTistoryUrls, _ := getTistoryWithCDNUrls(foundUrl)
				if len(finalTistoryUrls) > 0 {
					for finalTistoryUrl, _ := range finalTistoryUrls {
						foundFilename := s.AttrOr("filename", "")
						links[finalTistoryUrl] = foundFilename
					}
				}
			} else if isTistoryUrl == true {
				finalTistoryUrls, _ := getTistoryUrls(foundUrl)
				if len(finalTistoryUrls) > 0 {
					for finalTistoryUrl, _ := range finalTistoryUrls {
						foundFilename := s.AttrOr("filename", "")
						links[finalTistoryUrl] = foundFilename
					}
				}
			}
		}
	})

	if len(links) > 0 {
		fmt.Printf("[%s] Found tistory album with %d images (url: %s)\n", time.Now().Format(time.Stamp), len(links), url)
	}
	return links, nil
}

func getJson(url string, target interface{}) error {
	r, err := http.Get(url)
	if err != nil {
		return err
	}
	defer r.Body.Close()

	return json.NewDecoder(r.Body).Decode(target)
}

func getJsonWithHeaders(url string, target interface{}, headers map[string]string) error {
	client := &http.Client{}
	req, _ := http.NewRequest("GET", url, nil)

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	r, err := client.Do(req)
	if err != nil {
		return err
	}
	defer r.Body.Close()

	return json.NewDecoder(r.Body).Decode(target)
}

func getInstagramVideoUrl(url string) string {
	resp, err := http.Get(url)

	if err != nil {
		return ""
	}

	defer resp.Body.Close()
	z := html.NewTokenizer(resp.Body)

	for {
		tt := z.Next()
		switch {
		case tt == html.ErrorToken:
			return ""
		}
		if tt == html.StartTagToken || tt == html.SelfClosingTagToken {
			t := z.Token()
			if t.Data == "meta" {
				for _, a := range t.Attr {
					if a.Key == "property" {
						if a.Val == "og:video" || a.Val == "og:video:secure_url" {
							for _, at := range t.Attr {
								if at.Key == "content" {
									return at.Val
								}
							}
						}
					}
				}
			}
		}
	}

}

func filenameFromUrl(dUrl string) string {
	base := path.Base(dUrl)
	parts := strings.Split(base, "?")
	return parts[0]
}

func startDownload(dUrl string, filename string, path string, channelId string, userId string, fileTime time.Time) {
	for i := 0; i < MaxDownloadRetries; i++ {
		if downloadFromUrl(dUrl, filename, path, channelId, userId, fileTime) {
			break
		} else {
			time.Sleep(5 * time.Second)
		}
	}
}

func downloadFromUrl(dUrl string, filename string, path string, channelId string, userId string, fileTime time.Time) bool {
	err := os.MkdirAll(path, 755)
	if err != nil {
		fmt.Println("Error while creating folder", path, "-", err)
		return false
	}

	client := new(http.Client)
	request, err := http.NewRequest("GET", dUrl, nil)
	if err != nil {
		fmt.Println("Error while downloading", dUrl, "-", err)
		return false
	}
	request.Header.Add("Accept-Encoding", "identity")
	response, err := client.Do(request)
	if err != nil {
		fmt.Println("Error while downloading", dUrl, "-", err)
		return false
	}
	defer response.Body.Close()

	if filename == "" {
		filename = filenameFromUrl(response.Request.URL.String())
		for key, iHeader := range response.Header {
			if key == "Content-Disposition" {
				_, params, err := mime.ParseMediaType(iHeader[0])
				if err == nil {
					newFilename, err := url.QueryUnescape(params["filename"])
					if err != nil {
						newFilename = params["filename"]
					}
					if newFilename != "" {
						filename = newFilename
					}
				}
			}
		}
	}

	completePath := path + string(os.PathSeparator) + filename
	if _, err := os.Stat(completePath); err == nil {
		tmpPath := completePath
		i := 1
		for {
			completePath = tmpPath[0:len(tmpPath)-len(filepath.Ext(tmpPath))] +
				"-" + strconv.Itoa(i) + filepath.Ext(tmpPath)
			if _, err := os.Stat(completePath); os.IsNotExist(err) {
				break
			}
			i = i + 1
		}
		fmt.Printf("[%s] Saving possible duplicate (filenames match): %s to %s\n", time.Now().Format(time.Stamp), tmpPath, completePath)
	}

	bodyOfResp, err := ioutil.ReadAll(response.Body)
	if err != nil {
		fmt.Println("Could not read response", dUrl, "-", err)
		return false
	}
	contentType := http.DetectContentType(bodyOfResp)
	contentTypeParts := strings.Split(contentType, "/")
	if contentTypeParts[0] != "image" && contentTypeParts[0] != "video" {
		fmt.Println("No image or video found at", dUrl)
		return true
	}

	err = ioutil.WriteFile(completePath, bodyOfResp, 0644)
	if err != nil {
		fmt.Println("Error while writing to disk", dUrl, "-", err)
		return false
	}

	err = os.Chtimes(completePath, fileTime, fileTime)
	if err != nil {
		fmt.Println("Error while changing date", dUrl, "-", err)
	}

	fmt.Printf("[%s] Downloaded url: %s to %s\n", time.Now().Format(time.Stamp), dUrl, completePath)
	err = insertDownloadedImage(&DownloadedImage{Url: dUrl, Time: time.Now(), Destination: completePath, ChannelId: channelId, UserId: userId})
	if err != nil {
		fmt.Println("Error while writing to database", err)
	}

	updateDiscordStatus()
	return true
}

type DownloadedImage struct {
	Url         string
	Time        time.Time
	Destination string
	ChannelId   string
	UserId      string
}

func insertDownloadedImage(downloadedImage *DownloadedImage) error {
	_, err := myDB.Use("Downloads").Insert(map[string]interface{}{
		"Url":         downloadedImage.Url,
		"Time":        downloadedImage.Time.String(),
		"Destination": downloadedImage.Destination,
		"ChannelId":   downloadedImage.ChannelId,
		"UserId":      downloadedImage.UserId,
	})
	return err
}

func findDownloadedImageById(id int) *DownloadedImage {
	downloads := myDB.Use("Downloads")
	//var query interface{}
	//json.Unmarshal([]byte(fmt.Sprintf(`[{"eq": "%d", "in": ["Id"]}]`, id)), &query)
	//queryResult := make(map[int]struct{})
	//db.EvalQuery(query, myDB.Use("Downloads"), &queryResult)

	readBack, err := downloads.Read(id)
	if err != nil {
		fmt.Println(err)
	}
	timeT, err := time.Parse("2006-01-02 15:04:05.999999999 -0700 MST", readBack["Time"].(string))
	if err != nil {
		fmt.Println(err)
	}
	return &DownloadedImage{
		Url:         readBack["Url"].(string),
		Time:        timeT,
		Destination: readBack["Destination"].(string),
		ChannelId:   readBack["ChannelId"].(string),
		UserId:      readBack["UserId"].(string),
	}
}

func findDownloadedImageByUrl(url string) *DownloadedImage {
	var query interface{}
	json.Unmarshal([]byte(fmt.Sprintf(`[{"eq": "%s", "in": ["Url"]}]`, url)), &query)
	queryResult := make(map[int]struct{})
	db.EvalQuery(query, myDB.Use("Downloads"), &queryResult)

	for id := range queryResult {
		return findDownloadedImageById(id)
	}
	return nil
}

func countDownloadedImages() int {
	i := 0
	myDB.Use("Downloads").ForEachDoc(func(id int, docContent []byte) (willMoveOn bool) {
		//fmt.Printf("%v\n", findDownloadedImageById(id))
		i++
		return true
	})
	return i
	// fmt.Println(myDB.Use("Downloads").ApproxDocCount()) TODO?
}

// http://stackoverflow.com/a/18695740/1443726
func sortStringIntMapByValue(m map[string]int) PairList {
	pl := make(PairList, len(m))
	i := 0
	for k, v := range m {
		pl[i] = Pair{k, v}
		i++
	}
	sort.Sort(sort.Reverse(pl))
	return pl
}

type Pair struct {
	Key   string
	Value int
}

type PairList []Pair

func (p PairList) Len() int           { return len(p) }
func (p PairList) Less(i, j int) bool { return p[i].Value < p[j].Value }
func (p PairList) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }

func updateDiscordStatus() {
	dg.UpdateStatus(0, fmt.Sprintf("%d pictures downloaded", countDownloadedImages()))
}
